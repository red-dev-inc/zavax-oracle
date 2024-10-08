# 

# ZavaX Oracle Servers Penetration Testing Report

## Executive Summary
All servers were tested with multiple scanning and exploitation tools. All problems reported were verified to be false positives.


## **Objective**

This report details the pentesting activities performed on the servers  for the **ZavaX Oracle** project. The objective of the analysis was to identify open **ports**, active **services**, and possible **vulnerabilities** associated with these services, in order to strengthen the security of the systems.

**List of analyzed servers:**

* **redbridge01** - 68.175.134.60 (New York Location 1)  
* **redbridge02** - 67.241.72.63 (New York Location 2)  
* **redbridge03** - 45.77.105.206 (Vultr, New Jersey)  
* **redbridge04 / zavax-oracle.red.dev** - 96.30.192.77 (Vultr, Atlanta)  
* **redbridge05** - 216.128.137.36 (Vultr, Dallas)  
* **redbridge06** - 45.76.27.244 (Vultr, Chicago)  
* **redbridge07** - 45.77.161.62 (Vultr, Miami)  
* **redbridge08** - 149.28.219.251 (Vultr, Silicon Valley)


## **Methodology**

The analysis focused on identifying vulnerabilities related to open ports and the services running on those ports. The main tools used include:

* **Nmap** for port and service identification.  
* **Metasploit** for the exploitation of known vulnerabilities.  
* **Exploit-DB and CVE** to validate findings.  
* **Knockpy** subdomain scanning

## **Penetration Testing Phases**

* **Port and Service Enumeration:** An initial scan was performed to detect open ports and associated services, such as **HTTP, HTTPS, SSH, FTP**.

* **Vulnerability Identification:** Information on possible vulnerabilities was sought based on the versions of the services discovered.

## **Results of Analysis**

### Server: redbridge01 (45.77.105.206 - New York Location 1)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http), 443(https), 5060(sip), 8080(http-proxy), 8233(tcpwrapped), 9651(unknown), 14851(ssh)  
* **Detected Services:**   
  * **SSH:** OpenSSH 8.9p1 Ubuntu 3ubuntu0.10 (Ubuntu Linux; protocol 2.0)  
* **Vulnerabilities found (Note: Verified as a false positive):**   
  * The latest version of **OpenSSH is 9.9** on port **14851**, multiple vulnerabilities have been found with the **msconsole** tool. It is recommended to **upgrade** from version **8.9 to 9.9**.

### Server: redbridge02 (67.241.72.63 - New York Location 2)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http), 443(https), 5060(sip), 8080(http-proxy), 8233(tcpwrapped), 9651(unknown), 14851(ssh)  
* **Detected Services:**   
  * **SSH:** n  
  *  Ubuntu 3ubuntu0.10 (Ubuntu Linux; protocol 2.0)  
* **Vulnerabilities found (Note: Verified as a false positive):**   
  * The latest version of **OpenSSH is 9.9** on port **14851**, multiple vulnerabilities have been found with the msconsole tool. It is recommended to **upgrade** from version **8.9 to 9.9**.

### Server: redbridge03 (45.77.105.206 - New Jersey)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http?), 443(https?), 5060(sip?), 8080(http-proxy)  
* **Detected Services:** No accuracy of services. Possibly the firewall prevents it, or there are no open services.  
* **Vulnerabilities found :** No vulnerabilities


### Server: redbridge04 / zavax-oracle.red.dev (96.30.192.77 - Atlanta)

* **Subdomains Found:** None                                 
* **Open Ports:** 443 (HTTPS), 14851(SSH)  
* **Detected Services:**  
  * **HTTP/HTTPS:**  OpenResty web app server 1.25.3.2  
  * **SSH:** OpenSSH 9.6p1 Ubuntu 3ubuntu13.5 (Ubuntu Linux; protocol 2.0)  
* **Detected Technologies (web):**  
  * Express  
  * Nginx  
  * OpenResty 1.25.3.2  
  * Node  
  * Bootstrap  
* **Vulnerabilities found (Note: Verified as false positives):**   
  * **OpenSSH** version **9.6p1** has been found in the ssh service. It is recommended to upgrade immediately, as there are vulnerabilities publicly available on the Internet.  
  * **CVE2011-3192:** The Apache web server is vulnerable to a denial of service attack when numerous overlapping byte ranges are requested.  
    * Note:   
    * References:  
      * https://www.tenable.com/plugins/nessus/55976  
      * [https://seclists.org/fulldisclosure/2011/Aug/175](https://seclists.org/fulldisclosure/2011/Aug/175)  
      * [https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2011-3192](https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2011-3192)

### Server: redbridge05 (216.128.137.36 - Dallas)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http?), 443(https?), 5060(sip?), 8080(http-proxy)  
* **Detected Services:** No accuracy of services. Possibly the firewall prevents it, or there are no open services.  
* **Vulnerabilities found:** No vulnerabilities

### Server: redbridge06 (45.76.27.244 - Chicago)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http?), 443(https?), 5060(sip?), 8080(http-proxy)  
* **Detected Services:** No accuracy of services. Possibly the firewall prevents it, or there are no open services.  
* **Vulnerabilities found:** No vulnerabilities

### Server: redbridge07 (45.77.161.62 - Miami)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http?), 443(https?), 5060(sip?), 8080(http-proxy)  
* **Detected Services:** No accuracy of services. Possibly the firewall prevents it, or there are no open services.  
* **Vulnerabilities found:** No vulnerabilities

### Server: redbridge08 (149.28.219.251 - Silicon Valley)

* **Subdomains Found:** None                                 
* **Open Ports:** 53(dns), 80(http?), 443(ssl/https), 5060(sip?), 8080(http-proxy)  
* **Detected Services:**  
  * **HTTPS:**  OpenResty web app server 1.25.3.2  
    * Supported Methods: GET HEAD POST OPTIONS  
    * http-title: ZavaX Oracle  
* **Vulnerabilities found (Note: Verified as a false positive):**   
  * **CVE2011-3192:** The Apache web server is vulnerable to a denial of service attack when numerous overlapping byte ranges are requested.  
    * References:  
      * https://www.tenable.com/plugins/nessus/55976  
      * [https://seclists.org/fulldisclosure/2011/Aug/175](https://seclists.org/fulldisclosure/2011/Aug/175)  
      * [https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2011-3192](https://cve.mitre.org/cgi-bin/cvename.cgi?name=CVE-2011-3192)

## Notes Regarding False Positives
### SSH
See [https://ubuntu.com/security/CVE-2024-6387](https://ubuntu.com/security/CVE-2024-6387). Checking versions of SSH on *redbridgeXX* servers, they are either *1:8.9p1-3ubuntu0.10* or *1:8.9p1-3ubuntu0.11*. Both have been fixed.

### Apache
Apache web server is has been verified to not be installed on any *redbridgeXX* server.